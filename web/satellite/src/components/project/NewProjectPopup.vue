// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

<template>
    <div class="new-project-popup-container">
        <div class="new-project-popup" id="newProjectPopup">
            <div class="new-project-popup__info-panel-container">
                <h2 class="new-project-popup__info-panel-container__main-label-text">Create New Project</h2>
                <img src="@/../static/images/dashboard/CreateNewProject.png" alt="">
            </div>
            <div class="new-project-popup__form-container">
                <HeaderedInput
                    label="Project Name"
                    additionalLabel="Up To 20 Characters"
                    placeholder="Enter Project Name"
                    class="full-input"
                    width="100%"
                    :error="nameError"
                    @setData="setProjectName">
                </HeaderedInput>
                <HeaderedInput
                    label="Description"
                    placeholder="Enter Project Description"
                    class="full-input"
                    isMultiline
                    height="100px"
                    width="100%"
                    @setData="setProjectDescription">
                </HeaderedInput>
                <div class="new-project-popup__form-container__terms-area">
                    <Checkbox class="new-project-popup__form-container__terms-area__checkbox"
                              @setData="setTermsAccepted"
                              :isCheckboxError="termsAcceptedError"/>
                    <h2>I agree to the Storj Bridge Hosting <a>Terms & Conditions</a></h2>
                </div>
                <div class="new-project-popup__form-container__button-container">
                    <Button label="Cancel" width="205px" height="48px" :onPress="onCloseClick" isWhite/>
                    <Button label="Create Project" width="205px" height="48px" :onPress="createProject"/>
                </div>
            </div>
            <div class="new-project-popup__close-cross-container">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" v-on:click="onCloseClick">
                    <path d="M15.7071 1.70711C16.0976 1.31658 16.0976 0.683417 15.7071 0.292893C15.3166 -0.0976311 14.6834 -0.0976311 14.2929 0.292893L15.7071 1.70711ZM0.292893 14.2929C-0.0976311 14.6834 -0.0976311 15.3166 0.292893 15.7071C0.683417 16.0976 1.31658 16.0976 1.70711 15.7071L0.292893 14.2929ZM1.70711 0.292893C1.31658 -0.0976311 0.683417 -0.0976311 0.292893 0.292893C-0.0976311 0.683417 -0.0976311 1.31658 0.292893 1.70711L1.70711 0.292893ZM14.2929 15.7071C14.6834 16.0976 15.3166 16.0976 15.7071 15.7071C16.0976 15.3166 16.0976 14.6834 15.7071 14.2929L14.2929 15.7071ZM14.2929 0.292893L0.292893 14.2929L1.70711 15.7071L15.7071 1.70711L14.2929 0.292893ZM0.292893 1.70711L14.2929 15.7071L15.7071 14.2929L1.70711 0.292893L0.292893 1.70711Z" fill="#384B65"/>
                </svg>
            </div>
        </div>
    </div>
</template>

<script lang="ts">
import { Component, Vue } from 'vue-property-decorator';
import HeaderedInput from '@/components/common/HeaderedInput.vue';
import Checkbox from '@/components/common/Checkbox.vue';
import Button from '@/components/common/Button.vue';
import { APP_STATE_ACTIONS, NOTIFICATION_ACTIONS, PROJETS_ACTIONS } from '@/utils/constants/actionNames';
import { validateProjectName } from '@/utils/validation';

@Component(
    {
        data: function () {
            return {
                projectName: '',
                description: '',
                isTermsAccepted: false,
                termsAcceptedError: false,
                nameError: '',
            };
        },
        methods: {
            setProjectName: function (value: string): void {
                this.$data.projectName = value;
                this.$data.nameError = '';
            },
            setProjectDescription: function (value: string): void {
                this.$data.description = value;
            },
            setTermsAccepted: function (value: boolean): void {
                this.$data.isTermsAccepted = value;
                this.$data.termsAcceptedError = false;
            },
            onCloseClick: function (): void {
                this.$store.dispatch(APP_STATE_ACTIONS.TOGGLE_NEW_PROJ);
            },
            createProject: async function (): Promise<any> {
                let projectName = this.$data.projectName.trim();

                if (!this.$data.isTermsAccepted) {
                    this.$data.termsAcceptedError = true;
                }

                if (!validateProjectName(projectName)) {
                    this.$data.nameError = 'Name for project is invalid!';
                }

                if (projectName.length > 20) {
                    this.$data.nameError = 'Name should be less than 21 character!';
                }

                if (this.$data.nameError || this.$data.termsAcceptedError) {
                    return;
                }

                let response = await this.$store.dispatch(PROJETS_ACTIONS.CREATE, {
                    name: projectName,
                    description: this.$data.description,
                    isTermsAccepted: this.$data.isTermsAccepted
                });

                if (!response.isSuccess) {
                    this.$store.dispatch(NOTIFICATION_ACTIONS.ERROR, response.errorMessage);

                    return;
                }

                this.$store.dispatch(NOTIFICATION_ACTIONS.SUCCESS, 'Project created successfully!');
                this.$store.dispatch(APP_STATE_ACTIONS.TOGGLE_NEW_PROJ);
            }
        },
        components: {
            HeaderedInput,
            Checkbox,
            Button
        }
    }
)

export default class NewProjectPopup extends Vue {
}
</script>

<style scoped lang="scss">
    .new-project-popup-container {
        position: absolute;
        top: 0;
        left: 0;
        right: 0;
        bottom: 0;
        background-color: rgba(134, 134, 148, 0.4);
        z-index: 1121;
        display: flex;
        justify-content: center;
        align-items: center;
    }
    .input-container.full-input {
        width: 100%;
    }
    .new-project-popup {
        width: 100%;
        max-width: 845px;
        height: 440px;
        background-color: #FFFFFF;
        border-radius: 6px;
        display: flex;
        flex-direction: row;
        align-items: center;
        position: relative;
        justify-content: center;
        padding: 100px 100px 100px 80px;

        &__info-panel-container {
             display: flex;
             flex-direction: column;
             justify-content: flex-start;
             align-items: center;
             margin-right: 55px;
             height: 535px;

            &__main-label-text {
                 font-family: 'montserrat_bold';
                 font-size: 32px;
                 line-height: 39px;
                 color: #384B65;
                 margin-bottom: 60px;
                 margin-top: 0;
            }
        }

        &__form-container {
             width: 100%;
             max-width: 520px;

            &__terms-area {
                 display: flex;
                 flex-direction: row;
                 justify-content: flex-start;
                 margin-top: 20px;

                &__checkbox {
                     align-self: center;
                };

                h2 {
                    font-family: 'montserrat_regular';
                    font-size: 14px;
                    line-height: 20px;
                    margin-top: 20px;
                    margin-left: 0;
                };
                a {
                    color: #2683FF;
                    font-family: 'montserrat_bold';
                }
            }

            &__button-container {
                 width: 100%;
                 display: flex;
                 flex-direction: row;
                 justify-content: space-between;
                 align-items: center;
                 margin-top: 30px;
            }
        }

        &__close-cross-container {
             display: flex;
             justify-content: center;
             align-items: flex-start;
             position: absolute;
             right: 30px;
            top: 40px;
            svg {
                cursor: pointer;
            }
        }
    }

    @media screen and (max-width: 720px) {
        .new-project-popup {

            &__info-panel-container {
                 display: none;

            }

            &__form-container {

                &__button-container {
                     width: 100%;
                }
            }
        }
    }
</style>
